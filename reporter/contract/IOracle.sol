// SPDX-License-Identifier: GPL-3.0
pragma solidity >=0.8.2 < 0.9.0;
/**
 * @dev Interface of the Oracle Contract
 */
interface IOracle {
    // A structure contains the round data that helps gomock to mock the oracle contract interfaces.
    struct RoundData {
        uint round;
        int256 price;
        uint timestamp;
        uint status;
    }
    /**
     * @notice Update the symbols to be requested.
     * Only effective at the next round.
     * Restricted to the operator account.
     * @dev emit {NewSymbols} event.
     */
    function setSymbols(string[] memory _symbols) external;
    /**
     * @notice Retrieve the lists of symbols to be voted on.
     * Need to be called by the Oracle Server as part of the init.
     */
    function getSymbols() external view returns(string[] memory _symbols);
    /**
     * @notice Vote for the prices with a commit-reveal scheme.
     *
     * @dev Emit a {Vote} event in case of succesful vote.
     *
     * @param _commit hash of the ABI packed-encoded prevotes to be
     * submitted the next voting round.
     * @param _reports list of prices to be voted on. Ordering must
     * respect the list of symbols returned by {getSymbols}.
     *
     */
    function vote(uint256 _commit, int256[] memory _reports, uint256 _salt ) external;
    /**
     * @notice Get data about a specific round, using the roundId.
     *
     * @return the round data include round, price, timestamp and status.
     */
    function getRoundData(uint256 _round, string memory _symbol) external view returns (RoundData memory);
    /**
     * @notice  Get data about the last round
     *
     * @return the round data include round, price, timestamp and status.
     */
    function latestRoundData(string memory _symbol) external view returns (RoundData memory);
    /**
    * @notice Retrieve the current voters in the committee.
    */
    function getVoters() external view returns(address[] memory);
    /**
     * @notice Retrieve the current round ID.
    */
    function getRound() external view returns (uint256);

    /**
     * @dev Emitted when a vote has been succesfully accounted after a {vote} call.
     */
    event Voted(address indexed _voter, int[] _votes);

    /**
     * @dev Emitted with a debug msg for oracle contract debugging.
     */
    event DebugEvent(string);

    /**
     * @dev Emitted when a vote has been succesfully accounted after a {vote} call.
     * round - the round at which new symbols are effective
     */
    event NewSymbols(string[] _symbols, uint256 round);

    /**
     * @dev Emitted when a new voting round is started.
     */
    event NewRound(uint256 round);
    //         [9] - [10] - [11]                -     [12]         - [13]
    // NewRound(3) -        NewSymbols(AUTUSD)  -    NewRound(4)   -Vote(AUTUSD)
    // Note : at init phase of the Oracle Server, you need to need to wait for NewRound
    // before voting to make sure that you have the correct symbols.
    // Init phase Oracle Server:
    // headBlock = get latest block number
    /**
     * @dev Emitted when a new committee is set. - not needed
     */
    //event NewCommittee(address[] committee);
}